# Architecture map

One Go process holds every account. Each account is a `client.Session` with its own WebSocket and its own
goroutines; the fleet manager starts and stops them and paces their logins; a script (a `bot.Bot`) drives a
session through the `actions` layer; the panel is an HTTP server in the same process that reads snapshots and
sends commands. A browser is only a viewer: it never holds a game connection.

```
browser <-HTTP/event stream-> controlpanel (Source) <-> fleet.Manager --one per account--> client.Session <-ws-> game server
                                                              |                               |
                                                        bot.Runner -> Bot.Loop -> runner -> actions -> Session.Send (ISAAC writer)
                                                                                    ^                         |
                                                                              gamestate.State  <-- read loop decodes packets
```

## The layers, bottom up

| Layer | Package(s) | What it owns | What it must not do |
|---|---|---|---|
| Primitives | `isaac`, `rsa`, `packet` | The stream cipher, textbook RSA and the packet reader/writer. | Know anything about the game. Each is checked against vectors from the reference client. |
| Protocol tables | `protocol/clientprot`, `protocol/serverprot`, `protocol/gen` | Opcode numbers and server packet sizes, generated from an Engine-TS checkout. | Be edited by hand. Regenerate instead. |
| Transport | `transport` | The WebSocket byte stream (subprotocol `binary`, a 30 s read timeout). | Interpret bytes. |
| Login | `login` | The exact login byte sequence, the RSA-encrypted block and the two ISAAC ciphers it hands off. | Retry or pace; callers get the raw response code. |
| Session | `client` | Transport, login, the read loop, keepalive, game state and anti-cheat counters as one live `Session`. | Decide what to do. |
| Game state | `gamestate` | Everything the server said: modals, inventories, position, skills, NPCs, players, locs, ground items, messages. `State.Handle` is the dispatcher. | Send anything. |
| Cache data | `archive`, `cachecfg`, `locindex`, `entityquery` | Interface components, object/NPC/loc names and options, the static map's locs, and a query builder over what is visible. | Hold per-account state: these are loaded once and shared. |
| Actions | `actions` | One function per thing an account can do (walk, op NPC/loc/obj, bank, trade, shop, chat dialogue, say, ...). Each writes exactly the bytes the real client writes, including the side packets that ride along. | Walk to the target. An action sends one packet; it does not route. |
| Navigation | `pathfind`, `webwalk`, `webwalk/live`, `webwalk/hops`, `locreach`, `doorescape` | A* over a baked collision pack; long-distance routes over a graph of doors, stairs, ladders and ships; the executor that follows a route; the tiles from which a loc can really be used. | Import `client` (planning is pure; `live` adapts it to a session). |
| Random events | `randomevents` | Detection and handling of the game's random events. The `Guardian` is what ordinary scripts yield to. | Let a script act on a modal it does not own. |
| Scripts | `bot`, `scripts/*plan`, `scripts/*runner` | See below. | Mix the three pieces. |
| Fleet | `fleet`, `config`, `settings` | Many sessions, per-address login pacing, reconnect, strict settings, per-script settings storage, snapshots. | Know what a script does. |
| Coordination | `mulebus`, `peerbank`, `muleledger` | An in-process message board between accounts, published bank contents, and a ledger of hand-overs. | Decide hand-over policy: policy belongs to scripts. |
| Panel | `controlpanel`, `controlpanelapp`, `cmd/rsb0t`, `cmd/controlpanel` | The `Source` abstraction (a real `FleetSource`, a simulated `FakeSource`), the HTTP API, telemetry, the embedded web UI, and the first-run setup. | Be required: every script runs without it. |
| Proof | `harness`, `cmd/*` | Shared plumbing for live proof tools, and the tools. | Print a verdict it cannot support. |

## Scripts: plan, runner, registration

```
internal/scripts/<name>plan/     pure decisions, no I/O                 unit-tested in milliseconds
internal/scripts/<name>runner/   live glue: reads state, acts           shared with cmd/ proof tools
internal/bot/<name>.go           Bot + settings + init() Register(...)  the only piece the fleet imports
```

`bot.Runner` calls `Loop` repeatedly until it returns an error or a stop is requested; it does **not** call a
script once per game tick. A script chooses its own pace through `ctx.Delay`, which a stop request cancels (a raw
`time.Sleep` cannot be stopped). A script yields to the random-event `Guardian` (`WaitForGuardian` in the
runner) before acting. `bot.Context` carries the session, the shared pack, loc index and config, the chat log,
the in-process message board, the account's settings and a logger.

The registry (`bot.Register`, `bot.New`, `bot.Names`) is how the fleet and the panel start a script by name
without importing it. `bot.SettingsSchema(name)` returns a typed schema for the scripts that declare one, which
is what lets the setup page draw a form; every other script is configured with a JSON object.

## How a packet travels

1. `transport` reads a frame. The read loop in `client` decrypts the opcode with the incoming ISAAC cipher,
   looks the size up in `serverprot.ServerProtSizes` (fixed, or length-prefixed), and reads the payload.
2. `gamestate.State.Handle` dispatches on the opcode and updates the model under a lock. Entity updates
   (`PLAYER_INFO`, `NPC_INFO`) are bit-packed and are only partly decoded; see [gotchas.md](gotchas.md).
3. Subscribers (`State.Subscribe`) are told. Snapshots for the panel are read from the same state.

## How an action travels

1. A runner reads state, asks its plan what to do, and calls `actions.X`.
2. `actions` builds the bytes the real client's `doAction` would write, adds the anti-cheat side packets that
   client sends (`internal/anticheat` counts operations the way the server expects), and calls `Session.Send`.
3. `Session.Send` writes through the outgoing ISAAC cipher. Nothing is acknowledged: an action returning means
   the bytes were written, not that the server did anything. Verify by reading state afterwards.

## What is shared and what is per account

Loaded once per process and shared read-only: the collision pack, the loc index, the config archive, the
interface archive, the webwalk graph. Per account: the session, its state, its bot and the bot's context,
its settings. Shared mutable coordination (the message board, the bank publications, the ledger) is explicit
and lives in the three coordination packages. Anything else shared and mutable is a bug.

## Configuration and persistence

- `fleet.config.json` is the roster: endpoints, login pace, accounts, their scripts and settings. Passwords are
  never in it. `cmd/rsb0t` keeps everything under one data folder (`rsb0t-data`, or `RSB0T_HOME` / `-data`).
- `internal/config` also reads a stricter `"version": 1` format; both are described in
  [fleet_config_reference.md](../fleet_config_reference.md). Loading is strict in both.
- The panel saves its own state (layout, statistics, heat map, bank view) beside the roster. Script settings
  are stored per account and script by `internal/settings`, written atomically (temp file and rename).

## Optional systems

The stream client renders an existing session through an installed provider, on demand, without a second game
login. CrowdPlay (`internal/crowdplay`, `internal/twitchplays`) lets chat drive an account and is off unless
configured. The ML lab (`internal/ml`) is offline replay and evaluation; nothing in it changes live policy.
`internal/sessionipc` is an experiment in splitting the connection owner from the script brain for hot reload;
the normal path does not use it.

## Where to start for a given question

| Question | Start here |
|---|---|
| A login fails or loops | `internal/login`, `internal/fleet/loginrate.go`, [gotchas.md](gotchas.md#login) |
| A script stops doing anything | its runner, then `internal/randomevents` (a modal it does not own?), then `internal/locreach` |
| An unreachable tile or a bad route | `internal/pathfind`, `internal/webwalk`, `internal/locreach` |
| A packet decodes wrong | `internal/gamestate`, `internal/protocol`, [protocol.md](protocol.md) |
| The panel shows wrong data | `internal/controlpanel/source.go`, `fleetsource.go`, `fake.go` |
| A setting is rejected | the script's `DecodeSettings`, `internal/config` |
