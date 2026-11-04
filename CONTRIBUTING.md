# Contributing to rsb0t-go

rsb0t-go (said "RS bot go") is meant to be forked, extended and improved. Bug reports, fixes, new scripts, panel
pages, documentation and better tools are all welcome, from people and from AI coding agents. This page is the
whole process.

> **Licence.** rsb0t-go is released under the [MIT License](LICENSE). By opening a pull request you agree that your
> contribution is released under the same licence.

## Ground rules

1. **Only your own accounts, only where the server allows automation.** Read the server's rules. Try things on a
   game server you run yourself first ([docs/agents/local_engine.md](docs/agents/local_engine.md)).
2. **No credentials, tokens, addresses or real account names** in code, tests, fixtures, docs, logs, issues or
   commit messages. Passwords come from the environment or a file outside the repository.
3. **Cheats stay local.** Anything that sends `::` commands takes `-enable-cheats` and goes through
   `internal/harness`; it never targets a public server.
4. **No evasion of a server's anti-abuse measures,** and nothing whose purpose is to harm other players or the
   server. Such contributions are not accepted.
5. **Be kind.** Disagree about code, not people. Reviewers are volunteers.

## Fork, branch, change, test, pull request

```sh
# 1. fork on GitHub, then
git clone https://github.com/<you>/rsb0t-go.git
cd rsb0t-go
git remote add upstream https://github.com/oldm8clint/rsb0t-go.git

# 2. branch from the current upstream main
git fetch upstream
git switch -c my-change upstream/main

# 3. work, then run the gates (read each exit code; do not pipe them)
go build ./... ; echo build=$?
go vet ./...   ; echo vet=$?
go test -p 1 ./... ; echo test=$?
gofmt -l <the files you changed>

# 4. rebase on upstream before you push, then open the pull request from your fork
git fetch upstream && git rebase upstream/main
git push origin my-change
```

Go **1.23** is the floor. Do not use anything newer (`os.Root`, `testing.B.Loop`, `sync.WaitGroup.Go`,
`testing/synctest`). Use `-p 1`: the suite is large and parallel packages make timing-sensitive tests flaky.

Commit subjects are one imperative sentence describing the change ("Stop the fisher walking to an empty spot").
Keep commits small and the history readable. Never force-push a branch other people have pulled.

## What kind of change is it?

| Change | What it needs |
|---|---|
| **A new script** | A `<name>plan` package (pure, table-tested, strict `DecodeSettings`), a `<name>runner` package, an `internal/bot/<name>.go` that registers it, a settings test, a README line and a settings doc entry. Walk through [docs/agents/adding_a_script.md](docs/agents/adding_a_script.md). Say how far you proved it: unit tests, a local engine, hours of running. |
| **A change to the wire protocol** | The reference vector (`*_reference_vector.ts` and its regenerated `test/fixtures/` file), a Go test that compares byte for byte, and whether you also ran it against a local engine and which engine revision. Opcode tables are regenerated, never hand-edited. [docs/agents/protocol.md](docs/agents/protocol.md). |
| **A bug fix** | A test that fails without the fix, or, if the bug needs a live server to show, the exact steps and the log line that proves it is fixed. Fix the cause in the narrowest layer. |
| **A panel or setup-page change** | Check it on `rsb0t demo` (a simulated fleet) and describe or screenshot the change. The web assets are embedded; there is no build step. |
| **A proof tool under `cmd/`** | Build it on `internal/harness`. Read [docs/trustworthy_harnesses.md](docs/trustworthy_harnesses.md): a tool that can print PASS without having checked anything is not accepted. |
| **Docs** | Keep them true. If your change makes a sentence wrong, fix the sentence in the same pull request. |

Tests that need real game data skip themselves when the data is missing. If your change touches walking,
doors or interaction, build the data and run those tests too ([docs/agents/testing.md](docs/agents/testing.md)),
and say so in the pull request.

## Pull request checklist

The pull request template asks for the same things.

- [ ] It does one thing, and the title says what.
- [ ] `go build ./...`, `go vet ./...` and `go test -p 1 ./...` all exit 0 (say if the real-data packs were set).
- [ ] New behaviour has tests; a bug fix has a test that fails without it.
- [ ] Settings stay strict: unknown keys are errors; `settings.example.json` and the docs agree with the code.
- [ ] Protocol changes include the reference vector and say whether they were run live.
- [ ] New scripts follow the plan / runner / registration split and log one clear line per behaviour.
- [ ] No credentials, personal data, addresses, private paths or real account names anywhere in the diff.
- [ ] Docs updated, and, if you learned something the hard way, `docs/agents/` too.
- [ ] You ran it, or you say you did not.

## Reporting bugs and asking for features

Use the issue templates. A good bug report says: what you ran (version or commit), the server (a local Engine-TS
or something else, and the revision), what you expected, what happened, and the relevant log lines with names
and addresses removed. For a feature, say what you want to do and why the current scripts do not do it.

If you find a security problem (for example a way to leak a stored password), do not open a public issue: use
GitHub's private vulnerability reporting for this repository.

## Working with AI coding agents

If you use an agent, point it at [AGENTS.md](AGENTS.md). It holds the rules and links to the architecture map,
the gotchas and the lessons. Review what an agent produces as you would any contributor's work: run the gates
yourself, and do not accept a claim of "tested" that does not say what was run. Agents and people are held to
the same checklist.

If a change taught you or your agent something this repository did not know, add it to
[docs/agents/](docs/agents/README.md) in the same pull request. That is how the project remembers.
