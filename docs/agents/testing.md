# Testing

## The gates

Run each one bare and read its exit code. A pipeline reports only its last command's status, so
`go build ./... | tail` has printed "ok" over a failed build.

```sh
go build ./... ; echo "build=$?"
go vet ./...   ; echo "vet=$?"
go test -p 1 ./... ; echo "test=$?"
```

- `-p 1` runs one package at a time. The suite is large and some packages are heavy; on a laptop that also runs
  a game server, parallel packages make timing-sensitive tests flaky and the machine unusable.
- For a small change, run the packages you touched first (`go test -p 1 ./internal/scripts/vialplan/`), then the
  full gates before you push.
- One test: `go test -run TestName ./internal/config/`. Use `-count=1` when you suspect a cached result.
- `gofmt -l <your files>`. With `core.autocrlf` on Windows it lists everything; compare before and after.
- Go 1.23 is the floor: if your toolchain is newer, do not use anything released after 1.23.
- Browser-side checks: `node tools/stream/verify-scene-state.mjs` and the other `verify-*` scripts under
  `tools/stream` (several need a browser and a harness; read their headers).

## Tests that skip, and what that costs you

Tests that need real game data skip themselves when the data is absent. That keeps a fresh clone green, and it
hides bugs: a race in a fake mule sat unseen for weeks behind a skip, and walking code is only exercised when
the collision pack is present. **A green run without the packs is not a statement about walking, doors,
interaction tiles or any script that navigates.** When you report a test result, say whether the packs were set.

| Variable | What it points at |
|---|---|
| `RSB0T_COLLISION_PACK` | the baked collision pack (`collision.lcnav` or `.lcnav.gz`) |
| `RS2BOT_TEST_COLLISION_PACK` | the same pack; two script-package tests read this spelling (with a letter O, a historical typo kept so those tests keep working) |
| `RSB0T_LOC_INDEX` | the static loc index (`locs.lcli.gz`) |
| `RSB0T_CONFIG_JAG` | the client's `config` archive from a built engine or downloaded from a server |
| `RSB0T_INTERFACE_ARCHIVE` | the client's `interface` archive |
| `RSB0T_CONTENT` | a checkout of the Content repository, for tests that read content scripts |
| `RSB0T_ERA_ASSETS`, `RSB0T_LOOT_STATE` | optional extra data for a few asset and migration tests |

## Building the packs from public sources

About five minutes, no private data. You need Git, Node or Bun, and Go.

```sh
git clone --depth 1 --branch 289 https://github.com/LostCityRS/Engine-TS.git engine
git clone --depth 1 --branch 289 https://github.com/LostCityRS/Content.git content        # sibling of engine
(cd engine && bun install --ignore-scripts && bun tools/pack/Build.ts)                     # packs the cache, ~25 s

# the collision pack is baked by a TypeScript tool in the reference client repository
# (tools/nav/build-collision.ts); there is no Go baker yet, see open_work.md
(cd <reference client> && bun install --ignore-scripts && \
   bun tools/nav/build-collision.ts --engine <abs>/engine --out <abs>/collision.lcnav)

go run ./cmd/locbake -engine <abs>/engine -config <abs>/engine/data/pack/client/config -out <abs>/locs.lcli.gz

export RSB0T_COLLISION_PACK=<abs>/collision.lcnav.gz  RS2BOT_TEST_COLLISION_PACK=<abs>/collision.lcnav.gz
export RSB0T_LOC_INDEX=<abs>/locs.lcli.gz
export RSB0T_CONFIG_JAG=<abs>/engine/data/pack/client/config
export RSB0T_CONTENT=<abs>/content
export RSB0T_INTERFACE_ARCHIVE=<abs>/engine/data/pack/client/interface
```

Notes:

- This is the public 289 content, not a modified lab engine; with it the whole suite passed in one verification,
  except tests that needed the optional asset variables.
- Keep the packs outside the repository (they are in `.gitignore`: `*.lcnav*`, `*.lcli*`, `*.jag`).
- Rebuild them when you change engines: a pack baked from one revision's maps can disagree with another's.
- Use absolute paths. Relative paths resolve from each test package's directory.

## Writing a good test

- **Pure first.** Put the decision in a plan package and test it with plain values. These tests are fast, need
  no data, and cover most of the logic.
- **Table tests with names.** A failing case should say which row. Include the boundary values and the
  nonsense input (empty, negative, unknown name).
- **Vectors for bytes.** Anything on the wire is compared against a fixture captured from the reference client
  ([protocol.md](protocol.md)).
- **A check must be able to fail.** Before you trust a new test, break the code on purpose and watch it fail. A
  precondition like `x != nil || true` is not a check.
- **Assert the effect, not the proxy.** XP gained, items changed, a kill confirmed, the tile reached. Not "the
  account is near the place" and not "the NPC is no longer visible" (it may have walked out of range).
- **Read state twice.** Before and after the action. A count that was already correct proves nothing.
- **No sleeps for synchronisation, no wall-clock thresholds.** Pass a clock or a `time.Time` in. Tests that
  depend on real time are the ones that flake when the machine is busy.
- **Skip loudly.** If a test needs data, `t.Skip` with the variable's name so the reason shows in `-v` output.
- **No credentials, addresses or names of real accounts in fixtures.** Use obviously fake names.

## Live proofs against a local engine

Unit tests prove decisions; a local engine proves the server agrees. The tools under `cmd/` that do this must
use `internal/harness`, and are read with the rules in [trustworthy_harnesses.md](../trustworthy_harnesses.md).
In short: setup is a precondition that voids the run when it fails (exit 2), every check prints the observation
that decided it, a run that asserted nothing fails, and cheats are turned back off before the part under test so
a recovery path cannot quietly rescue itself with a teleport a real server would never give it.

Starting a server: [local_engine.md](local_engine.md).

## Measuring a change

If you claim a change made something faster or better, measure the mechanism, not just the outcome. Throughput
over a running fleet has a noise floor high enough that most small improvements are invisible in an hour of log.
Measure the walk's length, the seconds per transition, the share of time in a phase: those have thousands of
samples. State the window, quote the mean and the spread (the median alone can disagree with the mean by enough
to turn "no effect" into a fake regression), and call an interim number interim.
