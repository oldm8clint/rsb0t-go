# Gotchas

Each entry is a trap that cost somebody time, with the symptom first. "Proven" means observed against a game
server; "read" means taken from source and not observed; the revision of the engine is Engine-TS branch 289
unless stated.

## Login

- **Login response 6 looks exactly like a bad RSA key and is usually the checksums.** The login block carries
  nine archive CRCs, and they must be the first 36 bytes of the server's own `/crc0` response. The engine checks
  them before it tries the RSA block and answers 6 on a mismatch. Fetch `/crc0` from the server you are logging
  into; do not reuse a table from another server or another build.
- **A server's RSA modulus changes when the server restarts.** There may be no key endpoint: the modulus can be
  read from the served `client.js` (`internal/fleet.FetchLoginKey`), which finds both a 250+ digit modulus
  (exponent 65537 unless the bundle states another) and a stock engine's `BigInt(modulus),BigInt(exponent)` pair
  (a 154-digit modulus with a 77-digit exponent). Do not assume 65537. A local
  engine's key comes from its own `private.pem`, whose large exponent OpenSSL 3.5 prints as a hex byte dump.
- **Response 5 means "already logged in".** The account is in use, possibly by you. Stop that account; do not
  retry in a loop.
- **Response 16 means too many login attempts.** Servers rate-limit logins per address; the limit is the
  server's own (one server was measured at about 29 a minute), so do not assume a number for another one.
  `internal/fleet/loginrate.go` paces every login in the process through one gate; do not add a
  second path that logs in without it.
- **Response 9 is a per-address ceiling on concurrent connections, not a rate limit.** Waiting does not clear
  it; releasing sessions does. Diagnose from a log of when it started, not from the number alone.
- **Response 21 is "you logged out of another world too recently"** and is the only response with a payload
  byte (seconds remaining; the default wait is 45 s from the previous logout). Staff accounts are exempt, so
  a local-engine hop test (where accounts are staff) cannot see it and says nothing about hopping timing.
- **Keepalive.** A stock Engine-TS logs a session out after about 60 s without client bytes. The real client
  sends `NO_TIMEOUT` (opcode 181); the session's keepalive goroutine does the same. A test that blocks the
  session's goroutines (a debugger, a long stop-the-world) looks like a random disconnect.
- **A stock Engine-TS on Windows serves HTTP and the WebSocket on port 80**, on other systems on whatever
  `WEB_PORT` you set (8888 in the examples in [local_engine.md](local_engine.md)). Tools default to
  `ws://localhost/` and `http://localhost/`; pass `-url` and `-http` for anything else.

## Protocol and data

- **Inventory ids are the object id plus one.** In `UPDATE_INV_FULL` and `UPDATE_INV_PARTIAL`, 0 means empty,
  so object 0 is sent as 1. Forgetting this puts every item one id off and looks like a name-table bug.
- **Cache bzip2 blobs have no header.** Prepend `BZh1` before handing them to `compress/bzip2`.
- **`PLAYER_INFO` and `NPC_INFO` are only partly decoded.** Extended data is walked for names, combat level,
  animations, face targets, hit marks and NPC overhead speech; other fields are skipped by length. If you need a
  field, add it with a captured packet as a test fixture, not by guessing the bit layout.
- **`UPDATE_ZONE_PARTIAL_ENCLOSED`** is decoded for locs and ground items. A loc you can see on the map but not
  in the live state may simply not have been in a zone update yet.
- **Regenerating the opcode tables.** `go run ./internal/protocol/gen -engine <Engine-TS checkout>` (flags in its
  doc comment). The reference generator in the TypeScript repo overwrites its committed tables when run
  directly; compare by calling its `generateProt()` from a throwaway script instead.
- **The reference is the authority, not the notes.** Some older comments say revision 274; the reference client
  sets `CLIENT_VERSION = 289`.
- **Two engine checkouts give two answers.** An unmodified Engine-TS 289 is the reference; a lab engine with local
  changes (extra cheats, a faster tick) serves your tests. Line numbers, available `::` commands and tick rate
  differ. Say which checkout a claim comes from, and never read a cheat's absence from the wrong one.
- **A faster tick rate changes every rate you measure.** A lab engine running at 200 ms per tick against the
  stock 600 ms means no wall-clock rate measured there is a rate on a real server.

## World interaction

- **`OpLocU` ("use item on loc") is not broken when it "does nothing".** The packet is byte-correct. There are
  four ordinary causes, and only the first two are silent:
  1. *The server has no loc of that type at that tile.* A hard-coded id at a coordinate goes stale or was never
     right. Resolve locs **by name** through the loc index. Re-reading your own client's loc index proves
     nothing: it is not what the server consults.
  2. *The account is standing where the engine's `reachedLoc` refuses.* The engine sets the interaction and runs
     the trigger only from a tile that test accepts, and with the client-side route finder on it will not walk
     the account into place. Loc shapes 10, 11 and 22 accept only the footprint plus the orthogonal sides the
     loc's `forceapproach` mask (rotated by the placement angle) leaves open: never a diagonal. Use
     `internal/locreach` rather than "walk to radius 1".
  3. *A content precondition refused it* with an ordinary game message nothing was reading. Read `MESSAGE_GAME`.
  4. *The backpack component is not visible to the server* (a brand-new account whose tabs are not set up). A
     dynamic `inventoryComponent()` returning -1 is the cheap client-side tell.
- **Reading chat is not optional.** After any interaction that appears to do nothing, the answer is usually a
  game message ("I can't reach that!", "You need level N", "Your inventory is too full"). A script that never
  reads `MESSAGE_GAME` cannot tell why it is stuck.
- **A modal swallows movement.** A message box that reopens after every Continue (for example "inventory too
  full") leaves an account that sends walks which never move. Log the open modal id when a walk makes no
  progress, and clear or satisfy the modal before walking.
- **A bank booth tile is not a standing tile.** A destination on the booth itself is an unwalkable tile in the
  collision pack, so every route to it plans as unreachable. Stand beside the booth (booths without a
  `forceapproach` mask are served from either side).
- **A travel node that is a *loc* is not an NPC.** A spirit tree is a loc named "Spirit tree", but a graph edge
  that treated it as an NPC hop never found it, and no hop ever succeeded. Look an entity up by name across the
  kinds that could match, preferring the one that has the op you want.
- **Doors and gates are dynamic.** Shared collision data is immutable and cannot know a door is closed. Plan
  through operable doors with a per-search overlay of straight/corner wall changes, then open the first
  blocked crossing; never send a hypothetical route blindly.
- **Clicking is not walking.** `actions` writes the click only. The reference client runs its own route finder
  alongside every interaction; this one does not. Walk first, then act, then verify.

## Chat

- **Public chat works both ways, but `internal/chatlog` does not see it.** Lines reach only accounts within 15
  tiles (heard at 15, silent at 16), because they ride `PLAYER_INFO`. The text comes back changed: the engine
  unpacks, word-filters and re-packs it, so it may be sentence-cased with a trailing space; compare trimmed and
  case-insensitively, never with `==`. The chat type follows the speaker's rights (`ChatPublicMod` for staff,
  `ChatPublic` otherwise), so accept both. There is no self-echo: a nil error from `SayPublic` proves only that
  bytes were written.
- **`internal/fleet` records only `MESSAGE_GAME` into `chatlog`.** Player speech never reaches `bot.Context`'s
  chat. A silent chat feed is not evidence nobody spoke. A script that needs speech subscribes with
  `State.Subscribe` itself.

## Random events

- **Scripts yield to the Guardian.** A random event (a strange box, a maze, a mime, a swarm) owns the
  account's modals until it is solved. A script that clicks through one wastes the event or locks itself out.
- **Name resolvers matter.** `randomevents.Detect` skips name-matched work when no resolver is registered, so a
  proof tool that omits the config archive makes events partly invisible. The signature is a handler that logs
  "handling ... still present" at a fixed cadence and never clears. Register the resolver and the interface
  archive in any tool that expects events to be solved.
- **`::forcerandom` is a silent no-op on Tutorial Island** (the event spawner returns early for accounts in the
  tutorial area). A test that starts there reproduces nothing and looks broken.
- **A stranded account needs somewhere to go.** Some failed events teleport the account to a pocket
  (`randomevents/failteleport.go` names them). A script that only handles the event and not the aftermath leaves
  the account standing in the pocket, so recovery has to be a plan of its own.

## Tooling and environment

- **Local engine accounts show `staffModLevel` 2** because the engine sends `min(level, 2)` on login, even when
  the value it assigned is higher. Never read that line as evidence that a cheat is ungated.
- **`::setvar` then an immediate reconnect loses the varp.** The save is only flushed through the logout trigger
  when the player `canAccess()`, and that is false behind a brand-new account's appearance screen. Accept the
  appearance first, then set the varp, then wait for the flush and assert on the engine's own acknowledgement.
- **Paths differ between machines.** The reference vector scripts import a sibling TypeScript checkout, and some
  tools default `-pack` to one contributor's path. Pass `-pack` and fix the import path locally; do not commit
  your own paths.
- **Windows shells.** Wrapping `go` in `cmd /c` from PowerShell has hung `go vet`. Run `go` directly, or from
  Git Bash. With `core.autocrlf` on, `gofmt -l` lists every file (it normalises to LF); compare the list at
  `HEAD` and after your change, and judge only the difference.
- **Do not exclude an account from a run by its array position.** A position-dependent `-count` broke every time
  an account was appended. Take the account out of the roster and pass `-count 0`.
- **Suite failures under load are not always your change.** Timing-sensitive tests fail when the machine is
  saturated. Re-run the failing package alone, at `-p 1`, before you debug a regression that may be CPU noise,
  and say that you did.
