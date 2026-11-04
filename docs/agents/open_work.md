# Open work: things to build

Pick one, say so in an issue so two people do not build the same thing, and send a pull request. Items are
roughly ordered from "good first change" to "big". Each says what done looks like. If you finish one, delete or
amend its entry in the same pull request.

## Good first changes

- **Fix dead links in `docs/`.** Some pages link to files that are not part of this repository. Write a small
  link checker (a Go test or a script), list the dead links, and either repoint or remove them. Done: the
  checker runs in CI and passes.
- **More typed settings forms.** Only some scripts (`bot.SettingsSchema`) declare a typed schema, so the setup
  page can draw a form for them; the rest take a JSON object. Add a schema for another script, copying
  `HerbCleaner`. Done: the setup page shows a form, and a test shows defaults, bounds and an unknown key.
- **Remove leftovers of a script that is not part of this release.** A few script settings still mention a
  swarm script that is not included, and do nothing here. Remove the keys (and keep the strict-settings tests
  green) or hide them behind a clear error. Done: no setting in `docs/bot_script_settings*.md` refers to it.
- **README screenshots and a short first-run walkthrough.**
- **Tests for partial decodes.** `PLAYER_INFO`/`NPC_INFO` extended data is only partly decoded. Capture a real
  packet containing a field that is skipped today, add it as a fixture, and decode it.

## Medium

- **A collision-pack baker in Go.** The collision pack is baked by a TypeScript tool in the reference client
  repository. A Go `cmd/` tool that bakes it from an Engine-TS 289 checkout would remove the last non-Go step
  from [testing.md](testing.md). Done: byte-identical output to the TypeScript baker on the public 289 maps (the
  pack loader's tile-by-tile tests are the oracle).
- **Test a login from the setup page.** Add a "test login" button that does one login, logs out, and reports the
  response in plain words. It must go through the login pacing gate and must never print the password.
- **OS keychain for passwords.** Credentials are stored in a file readable only by you. Use the Windows
  credential store, macOS Keychain or the Secret Service where available, with the file as the fallback.
- **Manage accounts inside the panel,** not only on the setup page and with `rsb0t account`.
- **Cross-platform CI for the panel.** The CI matrix builds and tests on Linux and Windows; add macOS, and a
  headless-browser smoke test of the panel against `rsb0t demo`.
- **Link checker and doc freshness.** A CI job that fails when a doc names a file or package that no longer
  exists.

## New scripts

The registered list is `grep -rn 'Register(' internal/bot/*.go`. Gaps worth filling, each as plan + runner +
registration ([adding_a_script.md](adding_a_script.md)): a general-purpose miner, a firemaker, a cook that is
not tied to a mule, more agility courses, prayer training beyond burying bones, a smithing script that smiths
rather than only smelts, a generic bank-and-sell loop. Each should state its settings, its stop conditions and how
far it was proven.

## Larger

- **More server revisions.** Any revision-289 server already works (`rsb0t server <address>`); other revisions
  do not. Revision 289 is hard-coded in places that should be a profile. A `server.profile` that carries the
  revision and its opcode tables would let a fork target another revision. Done: the protocol tests run against
  two profiles' vectors.
- **A login test on the setup page.** Check connection is read-only on purpose; an opt-in "try one login" would
  confirm the revision (response 6 or not) before the panel starts.
- **A trace-driven simulator.** Replay recorded packet streams into `gamestate` and a fake `actions.Session` so
  a script can be exercised without a server. The ML lab (`internal/ml`) has replay pieces to start from.
- **Reducing per-account memory.** `cmd/memprobe` measures the cost of each layer; a smaller footprint per
  account lets more accounts share one process.
- **The panel's remaining pages** (statistics, loot, heat map) on slower machines: profile with the panel's
  `-pprof` address and fix hot code.

## Rules for contributions here

Everything in [CONTRIBUTING.md](../../CONTRIBUTING.md) applies. Two in particular: do not point a new tool at a
server you do not run, and do not add a way to defeat a server's anti-abuse measures; contributions that do are
not accepted.
