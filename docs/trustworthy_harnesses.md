# Writing a live harness that can be believed

Every tool under `cmd/` prints a verdict somebody acts on: a fix gets deployed to the live
fleet, or a bug gets closed, because a harness said so. Over the first half of September 2026
several of them said so wrongly, and **every single case was caught by accident** rather than by
the tool noticing anything was wrong. This page lists the traps that produced those false
verdicts and the standard way to avoid them, which is `internal/harness`.

## The traps, each one a verdict this repo actually shipped

**1. The cheat gate was never opened.** `internal/actions.Cheat` returns `ErrCheatDisabled`
unless `actions.CheatEnabled` is true, and it defaults to false. Around 25 tools sent `::tele`,
`::give`, `::setstat` or `::forcerandom` without ever setting it. Where the error was checked the
tool died at its first cheat, so its historical results were from before the gate existed; where
the error was discarded (`_ = actions.Cheat(...)`) the setup silently did nothing and the run
measured an account that never reached its own start state. One tool could not spawn the event it
existed to solve. Another walked every route from wherever the previous run had left the account.

**2. The proxy an idle account also satisfies.** "XP went up" passed while the account was
stranded in the wrong city, because something else earned the XP. "Back within 10 tiles after 0s"
passed because the measurement was taken before the teleport landed. "The event cleared" passed
because a leftover monster from the previous trial cleared instead. A count that was already
correct before the test began was read as proof the withdrawal worked.

**3. The loose check that is true too early.** `OnCourse` is a bounding box the account is
already inside while still walking to the obstacle, so the run printed SUCCESS mid-route.
`InsideCourse` is the tight one. Prefer the check that is false until the thing has happened.

**4. The disappearance counted as the outcome.** An NPC that is no longer in `NPCs()` may have
died, or walked out of render range, despawned, or been taken by another account. Counting its
absence as a kill let a run pass with no confirmed death.

**5. The failure that was relabelled.** One tool printed a paragraph explaining that the feature
it exists to prove produced no observable effect, and then printed `ALL PASS`. Another exited 0
whatever its own success ratio was. Another exited 0 because the runner ended in the stopped
state rather than the error state, having banked nothing.

**6. The precondition failure that looked like a result.** A tool whose bot could not start
burned its full six-minute timeout and then printed an ordinary FAIL, indistinguishable from the
thing under test genuinely not working. A harness bug — asking a bank for more of a non-stacking
item than fits — was reported as a product failure.

## The traps that fail closed, and read as findings

Everything above is a check that passed when it should not have. This is the opposite failure, and
over one long session it cost more time than any false PASS: a check that **cannot tell "no problem"
from "I never actually looked"**. Both come out the same, and the empty reading is the more
convincing of the two, because it arrives dressed as a finding — a tool that registers nothing, a
gate that is clean, an ancestor that is not one. Several agents chased one of these, and every one
was caught by somebody re-running the command by hand rather than by the check noticing anything.

The remedy is the same every time: make the reading prove that something was actually looked at.

**Never pipe a gate, and echo its exit code.** A pipeline reports only its LAST command's status,
and `pipefail` is off by default, so `go build ./... | tail` printed `BUILD OK` on a build that had
failed, and `git rebase ... | tail && git commit` committed on top of a rebase that had failed. Run
each gate bare:

```sh
go build ./... ; echo "build=$?"
go vet ./...   ; echo "vet=$?"
go test ./...  ; echo "test=$?"
```

**`gofmt -l` exits 0 whether or not it listed a file.** It reports by printing names, not through
its status, so `gofmt -l ./internal && echo clean` prints `clean` immediately below the filenames it
just listed. Its count is not a target either: **every `.go` file in this checkout is CRLF**
(`core.autocrlf` is true and there is no `.gitattributes`) and gofmt normalises to LF, so it flags
all of them — 1043 of 1043 at this writing. The only question worth asking is whether your own edit
changed the flagged set, and the only honest way to ask it is to **stash the edit and re-run at
HEAD**: a `git show`-based "before" emits LF against a CRLF tree, so an untouched file reads as a
regression you caused.

**Compare the flagged list, not its length.** The count moves on its own, because the roster of
`.go` files grows: it was 1043 of 1043 when the paragraph above was written and 1051 of 1051 a
few days later. One agent watched `gofmt -l` go 1045 to 1050, read the five as its own damage and
started hunting a formatting error it had not made; diffing the two lists showed all five were new
`scarabplan` files that had arrived in a fast-forward. Sort both lists and `comm` them, and the
answer is the filenames rather than a number that several other people's commits also move.

**`awk` rewrites a CRLF file to LF, exactly as `sed` does.** It has been recommended on this
project as the byte-preserving way to read a file, and it is the opposite of one: in Git Bash
gawk strips the `\r` while splitting records, so the carriage returns never reach your program.
One `awk '{print}'` pass over `COLLAB.md` took it from 913 CR line endings to 1, silently
rewriting the whole file. Measured with `od -c` on a three-line, 20-byte CRLF file:

| Command | Result |
|---|---|
| `cat`, `head -3`, `tail -n +1` | 20 bytes, byte-identical |
| `awk '{print}'`, `awk 1`, `sed -n p`, `grep ''` | 17 bytes, every `\r` gone |
| `awk -b '{print}'` | 17 bytes: `--characters-as-bytes` does **not** help |
| `awk 'BEGIN{RS="\r\n";ORS="\r\n"}{print}'` | 19 bytes, mangled a third way |

The one form that round-trips is re-appending the ending yourself,
`awk 'BEGIN{ORS=""}{print $0 "\r\n"}'`, which is presumably what an earlier "CRLF-preserved via
awk, verified byte-for-byte" note was doing. Do not reach for it: it also appends CRLF to a final
line that had none, and converts a mixed-ending file to all-CRLF, both measured. To read part of a
file without touching its bytes, use `head`, `tail` or `cat`.

**`grep -c` exits 1 when the count is zero**, which ends an `&&` chain without running the rest of
it and leaves output that looks like an ordinary "not found". **A single-file `grep -c` also prints
no filename**, so an `awk -F:` parse of field 2 — correct over several files — comes back empty for
one. The two together produced a confident and entirely wrong finding: that `cmd/guidecrash`
registered neither a `NameResolver` nor an interface archive. It registers both, at lines 127 and 129.

**Count events, not the lines that mention them.** Log text is written to be read, so it repeats
itself, and every repetition inflates a `grep -c`:

- `hops.Handler` prefixes several lines per crossing with the gate's label, so counting the label
  reported **42 toll crossings for a run that crossed 5**. The dialogue choice is what costs the 10
  coins, so it is exactly one per crossing, and that run's step-off and arrival markers agreed, 5 of 5.
- A verdict's own summary quotes earlier trip lines back into the log, so naive greps said 5 trips,
  2 stops and 2 latches where the events were 4, 1 and 1.
- `marathonDeathReport` reprints up to `marathonRing` (60) preceding lines, indented, after every
  death, so a single death doubles every count taken over that log. Anchor on the `[bot] ` prefix
  that the runner's own lines carry.

A tally that counts events as they arrive beats any grep of the log it wrote.

**Assert what was gained, not where the account stood.** A location is not an activity, and a step
taken is not a result:

- "the finish walked to the rung's bank before logging out" **PASSED** on a run with **43 bank
  trips, 0 kills and melee XP frozen at 1,164** — the account had spent the whole eight minutes
  standing at that bank.
- `-mode marathon -rungsabove N` credited a rung the route merely passes through: the moss giant
  stand sits on the Varrock sewer route, so a run that only ever reached Defence 25 was credited
  with a rung that needs 45, because the per-rung test was "inside the leash and XP moved".
- `cmd/equiptest` still holds a precondition that cannot fail — `eq.Inventory() != nil || true`,
  with an empty `if` body — so it prints its success line whatever the game is doing.

XP gained and kills made are readings a stranded, idle or dead account cannot produce. And a check
that cannot fail is not a check: where a run could satisfy an assertion without the sampler ever
having ticked, put the number of observations into the assertion itself.

**Parse a ref before using it as one, and ask a binary what it is.** `git merge-base --is-ancestor`
handed an unparsed `vcs.revision=<hash>` string answers `fatal: Not a valid object name` and exits
**128**, where a true negative exits **1**. Inside `if ! git ...` or an `&&` chain the two are
indistinguishable, and it was read as a true negative twice in one session. Cut the value out
(`awk -F=`, `cut -d=`) and `git rev-parse` it first. To learn what a binary is, ask it — `go version
-m <exe>` prints `vcs.revision`, `vcs.time` and `vcs.modified` for that build — rather than
reconstructing it from history. And on a branch whose base has moved, `git diff origin/main`
compares the two endpoints, so everything committed to `main` since you branched shows up in your
diff inverted, as though you had deleted it; compare against the merge base
(`git diff origin/main...HEAD`).

**Say which engine checkout a line number comes from.** `rsb0t-go-engine/engine` is the lab engine
and `rsb0t/engine` is the read-only 289 reference. Their packed interface archives are byte-identical
(`data/pack/server/interface.dat` and `data/pack/client/interface` both compare equal), but the
sources are not: the lab engine carries a 13-line `::forcerandom` block in `ClientCheatHandler.ts`
that shifts every line below it (`setvar` is at 205 there and 192 in the reference), and it runs
`World.TICKRATE` at 200 ms against the stock 600 — so no wall-clock rate measured in the lab is a
production rate.

**Two environment traps that void a run without saying so.** `::forcerandom` is a silent no-op on
Tutorial Island: it runs `[proc,macro_event_general_spawn]`, which returns at once unless
`~macro_event_allowed` holds, and `~macro_event_area` answers false for `~in_tutorial_island`. A
throwaway that logs in at 3094,3106 reproduces nothing, and the tool looks broken. And
`staffModLevel` prints as **2** for every local account, because `World.ts:905` sends
`Math.min(player.staffModLevel, 2)` on login (the same line in both checkouts) while the value the
server assigned it can be 4 — never read that line as evidence that a cheat is ungated.

Two more traps of this family are already in [Rules of thumb](#rules-of-thumb) below: the
`::setvar`-then-reconnect varp loss, and the missing `-config` that leaves `internal/randomevents`
without a `NameResolver`.

## The standard shape

Use `internal/harness`. It answers traps 1, 2 and 6 directly.

```go
enableCheats := flag.Bool("enable-cheats", false, "LOCAL TEST ENGINE ONLY: ...")
flag.Parse()
harness.Cheats("mytest", *enableCheats, *wsURL, *httpBase)   // exits 2 if the flag is off
                                                             // refuses outright against example.invalid
v := harness.New("mytest")

// Setup is a precondition, not a result. A teleport is not done until the tile is observed.
if !v.RequireNoError("teleported to the start", harness.Teleport(s, start, 8*time.Second)) {
    v.Finish()   // exits 2: VOID, this run proves nothing either way
}

before := countOf(s, "Raw lobster")
runTheThingUnderTest()
after := countOf(s, "Raw lobster")

// The assertion carries the reading that justifies it.
v.Check(harness.CountChanged("raw lobster", before, after, 5))
v.Check(harness.MovedAtLeast(from, harness.MustTile(s), 20))
v.Finish()
```

`Verdict` enforces three things that are easy to get wrong by hand:

- **A run that asserted nothing is a failure**, not a pass. Setup steps do not count as
  evidence, so a tool whose checks were all skipped can no longer exit 0.
- **A failed `Require` voids the run** and exits **2**, deliberately distinct from a real
  failure's **1**. The thing under test never got its chance, so neither verdict is honest.
- **Every `Check` prints the observation that decided it**, so the summary line can be audited
  against the log rather than trusted.

Exit codes: `0` pass, `1` a real failure, `2` the run proves nothing (setup never held, or the
cheat gate was refused).

## Rules of thumb

- **Send no cheat whose error you discard.** If the setup depends on it, a failure is fatal; say
  which cheat failed.
- **Turn cheats back off before the part under test.** `cmd/labfleettest` and
  `cmd/fisherstrandtest` set `actions.CheatEnabled = false` once staging is done, so a recovery
  path cannot quietly rescue itself with a teleport production would never have. Copy that.
- **Read the state twice.** Before and after. A single reading of a count or a position cannot
  distinguish "it worked" from "it was already like that".
- **Make the check false until it is true.** Tight boxes, observed deaths, item deltas.
- **Never point a cheat-enabled tool at `wss://w1.example.invalid`.** `harness.Cheats` refuses, but
  the reason is worth knowing: `CLIENT_CHEAT` is an opcode a real client only ever sends from a
  staff-only admin entry, so transmitting it at all is a client fingerprint, whether or not the
  server honors the command.
- **Stage a fresh account's varps before the reconnect, and require the engine's acknowledgement.**
  `::setvar` followed by an immediate reconnect silently loses the varp. `World.processLogouts`
  only flushes the save through content's LOGOUT trigger when `canAccess()` is true, and that is
  false behind the design modal a brand-new account logs in behind — so the varp is set on a
  session that is never cleanly removed, and the next login reads an older save. The order that
  works is: accept the appearance first, then `::setvar`, then wait for the flush, and assert on
  the engine's own acknowledgement rather than on having sent the command.
- **A harness that omits `-config` makes `internal/randomevents` partly blind, and it looks like
  a solver bug.** `SetNameResolver`/`SetInterfaceArchive` are optional in most `cmd/` tools, and
  `Detect` silently skips the name-matched work when no resolver is registered. For the Mime that
  means `Event.NpcIndex` stays -1, so `HandleMime` takes its "NPC not found" branch and sleeps out
  the full 12s `mimeDialogDeadline` on every call without ever reading an emote or clicking. The
  signature is a run that logs `handling`/`still present after handling` forever at roughly 13s
  intervals — one `mimeDialogDeadline` apart — and never clears. That cadence is the tell: a Mime
  the Guardian is really solving clears in about four cycles. The live fleet registers both in
  `internal/fleet/fleet.go`, so this is a harness-configuration artefact, not a production bug.
- **If a tool's verdict cannot be made trustworthy cheaply, say so in its own doc comment**
  rather than leaving a confident-looking PASS in place. `cmd/npctest` and `cmd/oplocuprobe` are
  marked that way today: they are diagnostics whose output needs eyes on the full log.

## What the live fleet's own variance lets you claim

A live A/B on the production fleet is a measurement with a noise floor, and that floor is high
enough to make most efficiency claims unprovable by throughput. This is worth knowing BEFORE
running one, because the temptation afterwards is to quote whichever number moved.

**The fleet's catch rate has sd ≈ 200 catches/min minute to minute** (measured 2026-09-21 across
two builds, 63 and 31 steady minutes, 268 accounts). Whirlpool waves, random events, bank trips
and spot respawns all land unevenly. So:

| effect you are trying to see | minutes per arm for 80% power |
|---|---|
| 1% of fleet time (~13 catches/min) | ~60 h per arm |
| 0.17% (a real measured change) | **~2,300 h per arm** |
| 5% (~63 catches/min) | ~2.5 h per arm |

**Consequences, all of which this project has got wrong at least once:**

- **Throughput can only rule out a LARGE regression on a small change.** Reporting "XP/h went up
  after my fix" off an hour of fleet log is reporting noise unless the effect is several percent.
- **Measure the MECHANISM, not only the outcome.** For a change that shortens a walk, the
  believable numbers are the walk's own: tiles per hop, seconds per transition, share of fleet
  time. Those have thousands of samples an hour, not sixty.
- **Use the mean and state the spread.** The catch-rate distribution is skewed, so the median and
  the mean disagree. On 2026-09-21 a change whose medians read 1308 -> 1237 (a 5.4% "regression")
  had means of 1262 -> 1265: **0.1 sigma, no effect at all.** The median nearly went into a
  report as a regression that did not exist.
- **A fault COUNT is not a rate until you have looked at its distribution in time.** The same day,
  "checking for a stranding" appeared to rise 10/h -> 172/h, a 16x regression. 76 of the 84 lines
  fell in two minutes — one whirlpool wave — and the fleet's catch rate during those two minutes
  (1339 and 1446) was ABOVE the surrounding median of 1224. A burst divided by a window is not a
  rate. This is the third time this exact mistake is recorded in this repo; see COLLAB_ARCHIVE's
  woodcutting-stall retraction and the 17,802-failed-random-events entry.
- **A short window flatters a change, and the flattery is large.** The same 2026-09-21 change
  measured **0.68 percentage points** of fleet time saved over a 0.49 h window and **0.17** over
  0.95 h — the first number was four times the second, and it was the one that nearly went into a
  report. Take the reading when the window closes, not when it looks good; and if you quote an
  interim number, say it is interim.
- **Say which one you are claiming.** "The walk got 30% shorter and throughput is unchanged within
  0.1 sigma" is a complete, honest result. "It made the fleet faster" is not supportable from an
  hour of log, however true it may be.

## Tools that are still only as good as their log

These make no automated claim worth trusting on its own, and say so in their own doc comments:

| Tool | Why |
|---|---|
| `cmd/npctest` | sends `OpNpc`/`OpLoc` and cannot observe whether the server acted on them |
| `cmd/oplocuprobe` | it prints what each tile around a loc did; the verdict is the reader's, and it is only useful paired with an instrumented engine's own log |
| `cmd/iconsetup`, `cmd/iconslotdump` | stocking and dumping helpers, not proofs |
| `cmd/guidecrash`, `cmd/doordiag`, `cmd/npcscan` | diagnostics whose value is the printed detail |
