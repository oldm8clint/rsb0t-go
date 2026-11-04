# CLAUDE.md

Everything an agent needs to work on rsb0t-go ("RS bot go") is in **[AGENTS.md](AGENTS.md)**; read it first.
The detail lives in [docs/agents/](docs/agents/README.md).

@AGENTS.md

Claude Code specifics:

- Build and test with `go build ./... ; go vet ./... ; go test -p 1 ./...` and read each exit code. Do not pipe
  a gate into `tail` or `grep`.
- Prefer the Edit tool for source changes. If you script an edit, open files as UTF-8 and keep their line
  endings; the repository normalises to LF.
- Work in a branch of your fork, not on `main`. Keep scratch files, logs and test binaries outside the checkout.
- Stop a process you started by its recorded PID. Never kill by a command-line pattern: another project on the
  same machine may run the same launcher.
