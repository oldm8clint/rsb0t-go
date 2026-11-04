# Lessons

Engineering lessons from building and running this project, written so they transfer. Each one is a rule plus
the failure that taught it. The game-specific traps are in [gotchas.md](gotchas.md); how to write a believable
proof tool is [trustworthy_harnesses.md](../trustworthy_harnesses.md).

## 1. How checks lie

- **A harness that cannot fail is worse than none.** Tools reported PASS because a gate was never opened (cheats
  sent without enabling them, so setup silently did nothing), because a proxy was satisfied by an idle account
  (XP rose from something else; "within 10 tiles" measured before the teleport landed; the event "cleared"
  because a leftover monster from the last trial cleared instead), or because a precondition was never checked.
  Fix: a verdict type that cannot pass without a check that ran, a setup that voids the run (exit 2) rather than
  failing it (exit 1), and checks that are false until the thing has happened.
- **Assert the gain, not the position.** "The account walked to the bank" passed on a run with dozens of bank
  trips, zero kills and frozen XP: it had spent the run standing at that bank. A location is not an activity.
- **An empty reading may mean "I never looked".** A check that registers nothing, a gate that prints clean, an
  ancestor test that returns false: all can be a tool that did not run. Make the reading prove it looked: put
  the number of observations into the assertion.
- **Pipes eat exit codes.** `go build ./... | tail` printed OK over a failed build, and `git rebase ... | tail &&
  git commit` committed on top of a failed rebase. Run gates bare and echo the status.
- **Exit codes that mean two things.** `gofmt -l` exits 0 whether or not it listed files. `grep -c` exits 1 when
  the count is 0, which ends an `&&` chain. `git merge-base --is-ancestor` on an unparsed ref exits 128, which
  inside `if !` looks like an honest "no". Parse a ref before using it, and ask a binary what it is
  (`go version -m <exe>` prints the VCS revision it was built from) rather than reconstructing it from history.
- **Count events, not lines that mention them.** Log text repeats itself: a label printed on several lines per
  crossing reported 42 crossings for a run that made 5; a summary quoting earlier lines doubled counts; a
  death report that reprints the preceding lines doubled every count taken over it. A tally kept as events
  arrive beats a grep of the log it wrote. Anchor on a prefix only the real event line carries.
- **Say which checkout a line number comes from.** A reference engine and a modified lab engine differ by whole
  blocks, so the same function sits on different lines and a cheat exists in one and not the other.
- **Real-data tests that skip hide bugs.** A race sat unseen for weeks because the test that exposed it only
  ran when a data pack was configured. Say which packs a test result used.

## 2. Measuring

- **Variance sets what you can claim.** Throughput on a running fleet has a noise floor high enough that a
  one-percent improvement needs days of data per arm. So: throughput can only rule out a large regression;
  measure the **mechanism** (a walk's length, seconds per transition, share of time per phase), which has
  thousands of samples an hour; report mean and spread, because a skewed distribution makes the median disagree
  with the mean enough to invent a regression.
- **A burst divided by a window is not a rate.** A fault count that "rose 16x" was one wave in two minutes, and
  throughput during it was above normal. Look at the distribution in time before you quote a rate.
- **A short window flatters.** The same change measured four times better over half an hour than over an hour.
  Take the reading when the window closes; label interim numbers interim.
- **Sampling interval is part of the measurement.** Sampling position every 12 s and averaging across pauses
  produced "nobody ever runs" for accounts that were running; sampling every 3 s showed the truth. Write the
  window next to any speed.

## 3. Debugging

- **Investigate before editing.** Reproduce, rank hypotheses by evidence, change the narrowest layer that
  explains it. Several long hunts ended in "it was never a protocol bug": the packet was right, the loc was not
  there, the tile was not standable, or a game message nobody read explained everything.
- **Read the server's answer.** The game says why it refused; a bot that does not read game messages is blind to
  its own failures.
- **Re-run the claim by hand.** Several confident findings (a tool registers nothing; a build is clean; a
  branch is an ancestor) were caught by someone re-running the command rather than trusting the report.
- **Compare against the merge base.** On a branch whose base moved, a diff against the other branch's tip shows
  everything merged since you branched, inverted, as if you deleted it.
- **A retracted conclusion is a result.** Record that it was wrong and why, next to the conclusion, so nobody
  re-derives it.

## 4. Designing long-running behaviour

- **State the script learned must outlive the script's restarts.** A retry that stopped and recreated a
  sub-script threw away what it had learned (shelves found empty, trips already made), so it walked to the same
  empty shop again and again. Keep learned state in the owner that survives the restart.
- **Every wait and retry needs a bound, and a reason in the log.** A loop that cannot give up is a stuck
  account, and a log that does not say why it retried cannot be debugged.
- **Survival logic needs an exit, not only healing.** A retreat that ate all its food but never walked away from
  the danger died anyway. Check that "flee" moves the account somewhere the threat is not.
- **A modal can strand a script.** An open message box that reopens on every continue stops all movement; the
  script sees a walk that goes nowhere. Log the modal and resolve it first.
- **Make new behaviour fall back safely.** When a new path desyncs from the game, it should drop to the old,
  proven behaviour rather than loop.
- **One launcher path.** When several launchers each built their own command line, a restart silently lost
  options another launcher set. Put every optional feature behind one function that all paths call, and print
  each feature's on/off state with the reason.
- **Strict settings tie a build to its config.** Rejecting unknown keys means a config written for a newer build
  fails on an older one. Good: it is loud. Remember it when rolling back.

## 5. Operating a long-running multi-account process

(These apply to anyone who runs many accounts, on a local engine or elsewhere.)

- **Build, test and smoke-test the candidate first; only then stop the old process.** A restart script that
  throws after the stop leaves the service down. After a restart, grep its output for the line that says the
  new process started, and do not assume.
- **Verify the stop before the start.** Check that the old process and its ports are really closed. Never run
  two overlapping copies. Archive the old log rather than overwriting it.
- **Keep the pacing across restarts.** A burst of simultaneous logins is how a server's flood protection trips.
  Pace logins from one gate, and honour a cooldown after an unclean stop.
- **Verify the binary you launch.** Compare its hash to the one you built; build to a path outside the one in
  use so a failed build cannot replace a working binary.
- **Start requests carry their settings.** In the panel API, starting a script without its settings on an
  account whose script is assigned can delete the assignment. Pass the full settings every time.
- **Watch the output that matters, not just liveness.** A pack item that a script could not handle quietly
  halted a hand-off loop for hours while every process was "up" and the checks measured liveness. A health check
  that runs on a schedule should read the throughput of the thing the system exists to do (trades per hour, XP
  per hour), and reschedule itself, including while a long job runs.
- **Logs hold names and chat.** They stay out of version control; publish scrubbed evidence instead.
- **Account restrictions you cannot see.** Servers may restrict automation, and may treat accounts that share
  an owner, an address, a login pattern or a behaviour as related. Respect the server's rules, run only accounts
  you own, and do not design around a server's anti-abuse measures.

## 6. Working with many agents and many people

- **One writer per working tree.** Two agents editing one checkout see each other's half-finished hunks as a
  stranger's. Give every agent its own git worktree and its own branch, and say which files are yours.
- **Do not message a running worker to change its mind.** Messaging an agent that is mid-task through some
  orchestration paths resumes a *second copy* that edits the same files. Wait for its result, or stop it and
  restart with an edited brief.
- **Resuming parallel work replays it.** A resume that matches a cached prefix of steps will re-run finished
  steps if parallel branches interleave differently the second time. Run independent lanes as separate
  sequential jobs, and check `git status` in every lane before resuming.
- **Claim, work, log.** Say what you are touching before you touch it, commit and push the claim, and when done
  record what you actually did and found, with the specific facts the next person needs.
- **Pull before every push; never force-push; never rewrite shared history.**
- **Brief like a peer.** An agent knows only what you wrote: state the goal, what has been ruled out, which files
  matter, and the resource limits. Give an agent a plan, not a hope.
- **Cap the lanes.** Seven agents building at once on a four-core machine pinned the CPU for everyone. Three or
  four is the limit that kept the machine usable.

## 7. Machines and processes

- **Tools can eat the machine.** A log-replay tool grew to 17 GB; test binaries that allocated tens of GB
  exhausted the page file and a service crashed with "out of memory". Cap each tool (about 2 GB), stream logs
  instead of loading them, set a temp directory on a disk with room (`GOTMPDIR`), and keep a watcher that kills
  oversized processes and also looks at committed memory, not only working set.
- **Do not lower a process's priority to make room.** Profile (the panel accepts a `-pprof` address) and fix
  the hot code.
- **Orphans.** On Windows with Git Bash, a background `tail -F | grep | awk` pipeline outlives the monitor that
  started it. Dozens of them pinned the CPU for a day. Use bounded checks (`tail -c` of the last megabytes and
  exit) or put a `timeout` on the command, and look for leftovers after a monitor ends.
- **Kill by PID, not by pattern.** A `tsx src/app.ts` pattern kill meant for one engine also killed a different
  project's engine on the same machine. Save the PID when you start a process, stop exactly that, and print the
  full command line of anything you are about to stop.
- **Detach long-lived processes properly.** On Windows, an app update can kill every process a session started;
  launching through WMI (`Win32_Process.Create`) keeps a service alive across it.
- **Stop the service before building beside it** if the machine is small. A build next to a loaded service takes
  forever and starves both.

## 8. Tools and text

- **Python defaults to the system code page on Windows.** Open source files with `encoding="utf-8"` and
  `newline=""` in edit scripts, or non-ASCII comments fail to match and invalid UTF-8 lands in Go sources. Prefer
  the editor tool for small edits. Check exact text with `cat -A` before a scripted replace on a CRLF file.
- **`sed` and `awk` rewrite CRLF files to LF.** `cat`, `head` and `tail` are byte-exact; `awk '{print}'` and
  `sed -n p` strip the `\r`, and `awk -b` does not help. To read part of a CRLF file without changing it, use
  `head`, `tail` or `cat`.
- **Do not trust a list you did not regenerate.** A count of scripts or tools in a document is stale the day
  after; `grep -rn 'Register(' internal/bot/*.go` is the list.
- **Sort and `comm` two lists** rather than comparing their lengths: the length moves for reasons that are not
  yours.

## 9. Keeping knowledge

- **Write the why next to the code.** Doc comments that record the failure behind a rule have saved the rule from
  being "simplified" away many times.
- **Keep start pages short and archives greppable.** One short page says what is current; the history lives in
  an archive you search, not read.
- **Name the strength of a claim.** Proven live, proven in a unit test, read from the source, guessed. A line
  that does not say is read as the strongest.
- **Record refuted ideas rather than deleting them.** "Tried, did not work, here is the measurement" prevents
  the third attempt.
- **Do not describe unbuilt work as finished.** Write unbuilt requirements down as requirements.
