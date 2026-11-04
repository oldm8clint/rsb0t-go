# Running against a local game server

Iterate against a server you run yourself. It is faster, it is safe, it lets you use staff commands to set a
test up, and nothing you break is anybody else's. A real server is for a final check on accounts you own, where
the server's rules allow automation.

## The server

rsb0t-go targets the **LostCityRS Engine-TS** game server and Content on branch **289**.

```sh
git clone --depth 1 --branch 289 https://github.com/LostCityRS/Engine-TS.git engine
git clone --depth 1 --branch 289 https://github.com/LostCityRS/Content.git content        # sibling of engine
cd engine
bun install --ignore-scripts
bun tools/pack/Build.ts                                                                    # ~25 s
```

`tools/lostcity/bootstrap.sh` (or `.ps1`) clones `LostCityRS/Server` into `third_party/lostcity`
for the offline ML lab; that is a separate thing and is not needed for these tests.

Start it. Engine-TS wants **Node 24** (it uses `#/...` subpath imports that Node 22 rejects, and Bun lacks
`node:sqlite`):

```sh
NODE_PRODUCTION=false LOGIN_SERVER=false NODE_MEMBERS=true EASY_STARTUP=true \
  BUILD_STARTUP=false BUILD_LIVE_RELOAD=false ./node_modules/.bin/tsx src/app.ts
```

With no login server and production off, **every username logs in (and is created on first login) with staff
level 4**, so every `::` command works. The environment variable names are the engine's own and may change;
read its README if one is rejected. By default it serves HTTP and the WebSocket together: on port 80 on
Windows, otherwise on the port its `WEB_PORT` names (8888 is common). Bind it to localhost and keep it there.

Tick rate: the stock tick is 600 ms. A faster tick makes tests quicker and makes every rate you measure wrong
for a real server; if you change it, write that next to your numbers.

## Pointing rsb0t-go at it

Check the connection first with the login tool:

```sh
go run ./cmd/logintest -url ws://localhost:8888/ -http http://localhost:8888/
```

`logintest` logs in with a throwaway account, fetches the server's real `/crc0` checksums and prints the first
game packets. It defaults to the key that a stock Engine-TS checkout ships; if you replaced the server's
`private.pem`, pass its modulus and exponent with `-mod` and `-exp`. If the response is 6, suspect the checksums
first ([gotchas.md](gotchas.md#login)).

For the program, point it at the engine like any other server:

```sh
rsb0t server localhost:8888        # or `rsb0t setup` and type localhost:8888 in the Server box
```

That checks `/crc0`, reads the login key from the engine's own `public/client/client.js` (a stock checkout's
154-digit modulus and 77-digit exponent are found), opens the game WebSocket and saves `wsURL` and `httpBase`.
If you replaced `data/config/private.pem` without rebuilding the client, `client.js` carries the old key; paste the
new one: `rsb0t server localhost:8888 -modulus <decimal> -exponent <decimal>`. The data files go in the roster too:

```json
{
  "wsURL": "ws://localhost:8888/",
  "httpBase": "http://localhost:8888/",
  "packPath": "/abs/collision.lcnav.gz",
  "locsPath": "/abs/locs.lcli.gz",
  "configPath": "/abs/config"
}
```

With `modulus` blank the key is read from the server's `client.js` at every start (and again after a response 6,
in case the server restarted with a new key). Set `modulus`/`exponent` only when that file does not carry the key
the server decrypts with. The three data files are built as in [testing.md](testing.md).

## Cheats

The `::tele`, `::give`, `::setstat`, `::setvar` and similar commands are for setting a test up. Rules:

- They are staff-only commands, and the opcode that carries them (`CLIENT_CHEAT`) is something a real client
  never sends from a normal account. They are **off** by default. A tool that needs them takes an explicit
  `-enable-cheats` flag and calls `harness.Cheats`, which refuses to run rather than silently send nothing, and
  refuses a public server's hostname outright.
- A cheat is a *precondition*, never part of the thing you are proving. Verify the setup took effect
  (`harness.Teleport` waits until the tile is observed), and turn cheats off again before the measured part.
- Do not discard a cheat's error. If the setup failed, the run measured an account that never reached its start
  state.
- Some commands exist only in a modified engine (for example a command that forces a random event). A stock
  checkout will not have them; check which engine you are on before concluding a command is missing, or that a
  line number in someone's note applies to you.
- Staff status changes behaviour: a staff account is exempt from the hop timer, and the engine reports its
  staff level as at most 2 on login even when it is higher. A local pass says little about those.

## Running a script on it

1. Start the engine, create the account by logging in once (or let `logintest` do it).
2. Add the account to the roster (`rsb0t account add`, or the setup page), choose the script and its settings.
3. For a walking script, set the three data paths above. Without them an account logs in and the panel works,
   but walking scripts say what they are missing.
4. Run `rsb0t`, open the panel, start the script, and read the log.
5. For a proof tool under `cmd/`, run it with `-enable-cheats` only if it needs setup, and read the verdict
   with [trustworthy_harnesses.md](../trustworthy_harnesses.md) in mind.

## Resource etiquette

A game server plus a Go build plus a browser is a lot for a laptop. Run **one** engine at a time, build and test
with `-p 1`, keep each tool under about 2 GB, and put scratch files, packs and test binaries outside the
checkout. Stop a process you started by its recorded PID, never by matching its command line: another project on
the machine may be using the same launcher.
